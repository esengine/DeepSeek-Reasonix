const setGrabState = function( ele, grabbed ){
  let ele0 = ele[0];

  if( !ele0 || ele0._private.grabbed === grabbed ){
    return;
  }

  ele0._private.grabbed = grabbed;
  ele.updateStyle( false );
};

export const setGrabbed = function( ele ){
  setGrabState( ele, true );
};

export const setFreed = function( ele ){
  setGrabState( ele, false );
};
